require 'spaceship'
require_relative 'portal/app_client'

def get_app(bundle_id)
  app = nil
  run_or_raise_preferred_error_message do
    app = Spaceship::Portal.app.find(bundle_id)
  end

  {
    id: app.app_id,
    bundleID: app.bundle_id,
    entitlements: app.details.features
  }
end

def check_bundleid(bundle_id, entitlements)
  app = nil
  run_or_raise_preferred_error_message do
    app = Spaceship::Portal.app.find(bundle_id)
  end

  Portal::AppClient.all_services_enabled?(app, entitlements)
end


# name = "Bitrise - (#{bundle_id.tr('.', ' ')})"
# Log.debug("registering app: #{name} with bundle id: (#{bundle_id})")

# app = nil
# run_or_raise_preferred_error_message { app = Spaceship::Portal.app.create!(bundle_id: bundle_id, name: name) }

# raise "failed to create app with bundle id: #{bundle_id}" unless app
# app